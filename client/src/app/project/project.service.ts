import { Injectable } from '@angular/core';
import { Project } from './project.material';

@Injectable({
  providedIn: 'root'
})
export class ProjectService {
  public projects: Project[] = [];

  constructor() {
    this.projects[0] = new Project('1', 'Test');
    this.projects[1] = new Project('2', 'foo');
    this.projects[2] = new Project('3', 'bar');
  }

  public getProjects() : Project[] {
    return this.projects;
  }

  public getProject(id: string) : Project {
    return this.projects.find(p => p.id == id);
  }
}
